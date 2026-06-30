it('keeps only items matching the status', function () {
    assert.deepEqual(
        filterByStatus(
            [{ id: 1, status: 'completed' }, { id: 2, status: 'active' }, { id: 3, status: 'completed' }],
            'completed'
        ),
        [{ id: 1, status: 'completed' }, { id: 3, status: 'completed' }]
    );
});

it('returns an empty array for non-array input', function () {
    assert.deepEqual(filterByStatus(null, 'completed'), []);
    assert.deepEqual(filterByStatus(undefined, 'completed'), []);
});
